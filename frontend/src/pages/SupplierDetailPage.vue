<template>
  <div v-loading="loading" class="supplier-detail">
    <template v-if="supplier">
      <header class="hero">
        <div class="hero-main">
          <h1>{{ displayName }} <small v-if="supplier.shortName && fullName !== displayName">{{ fullName }}</small></h1>
          <div class="hero-meta">
            <span>{{ supplier.code }}</span>
            <span v-if="supplier.countryCode">{{ countryName(supplier.countryCode, locale) }}</span>
            <el-tag v-for="role in supplier.businessTypes" :key="role" size="small" effect="plain">{{ optionLabel(role) }}</el-tag>
            <el-tag size="small" :type="supplier.status === 'ACTIVE' ? 'success' : 'info'">
              {{ supplier.status === 'ACTIVE' ? t('common.active') : t('common.inactive') }}
            </el-tag>
          </div>
        </div>
        <el-button size="small" class="hero-back" @click="backToSuppliers">← {{ t('suppliers.back') }}</el-button>
      </header>

      <nav class="section-nav" :aria-label="t('suppliers.profile')">
        <button v-for="section in sections" :key="section.key" type="button" :aria-current="activeSection === section.key ? 'location' : undefined" @click="selectSection(section.key)">
          {{ section.label }}
        </button>
      </nav>
      <div class="detail-sheet">
        <section id="supplier-profile" class="detail-section" tabindex="-1">
          <h2>{{ t('suppliers.profile') }}</h2>
          <el-form v-if="canWrite" label-position="top" size="small" :disabled="savingProfile" @submit.prevent="saveProfile">
            <SupplierProfileFields v-model="edit" :business-options="businessOptions" :payments="paymentOptions" :role-label="optionLabel" />
            <div v-if="dirty" class="profile-actions">
              <span v-if="profileError" class="error-text" role="alert">{{ profileError }}</span>
              <el-button size="small" :disabled="savingProfile" @click="cancelProfile">{{ t('common.cancel') }}</el-button>
              <el-button size="small" type="primary" native-type="submit" :loading="savingProfile">{{ t('common.save') }}</el-button>
            </div>
          </el-form>
          <dl v-else class="profile-read">
            <div v-for="field in readFields" :key="field.label" :class="{ 'full-line': field.wide }">
              <dt>{{ field.label }}</dt><dd>{{ field.value || '—' }}</dd>
            </div>
          </dl>
        </section>

        <section id="supplier-contacts" class="detail-section" tabindex="-1">
          <div class="section-head">
            <h2>{{ t('suppliers.contacts') }} <small>{{ contacts.length }}</small></h2>
            <el-button v-if="canWrite" link type="primary" @click="openContact">{{ t('suppliers.addContact') }}</el-button>
          </div>
          <p v-if="!contacts.length" class="empty">{{ t('suppliers.noRecords') }}</p>
          <div v-else class="record-list">
            <article v-for="row in contacts" :key="row.id" class="record-row">
              <div class="record-name"><strong>{{ row.name || '—' }}</strong><el-tag v-if="row.isPrimary" type="success" size="small">{{ t('suppliers.primary') }}</el-tag></div>
              <div class="record-detail"><span v-if="row.email">{{ row.email }}</span><span v-if="row.phone">{{ row.phone }}</span></div>
              <div class="record-meta">{{ [row.department, row.title].filter(Boolean).join(' · ') }}</div>
              <el-button v-if="canWrite" class="record-action" size="small" link type="danger" @click="removeContact(row)">{{ t('common.deactivate') }}</el-button>
            </article>
          </div>
        </section>

        <section id="supplier-owners" class="detail-section" tabindex="-1">
          <div class="section-head">
            <h2>{{ t('suppliers.owners') }} <small>{{ owners.length }}</small></h2>
            <el-button v-if="canEditOwners" link type="primary" @click="openOwner">{{ t('suppliers.addOwner') }}</el-button>
          </div>
          <p v-if="!owners.length" class="empty">{{ t('suppliers.noRecords') }}</p>
          <div v-else class="record-list">
            <article v-for="row in owners" :key="row.id" class="record-row">
              <div class="record-name"><strong>{{ row.employeeName }}</strong><el-tag v-if="row.isPrimary" type="success" size="small">{{ t('suppliers.primary') }}</el-tag></div>
              <div>{{ ownerOptions.find(option => option.code === row.responsibilityCode)?.label || row.responsibilityCode }}</div>
              <div class="record-meta">{{ [row.startDate, row.endDate].filter(Boolean).join(' ~ ') || '—' }}</div>
              <el-button v-if="canEditOwners" class="record-action" size="small" link type="danger" @click="removeOwner(row)">{{ t('common.deactivate') }}</el-button>
            </article>
          </div>
        </section>

        <section id="supplier-credit" class="detail-section" tabindex="-1">
          <h2>{{ t('credit.tab') }}</h2>
          <CreditRating party="suppliers" :party-id="supplier.id" :grade="supplier.creditGrade || ''" :graded-at="supplier.creditGradedAt || ''" :can-rate="canWrite" compact @rated="onRated" />
        </section>

        <section id="supplier-documents" class="detail-section" tabindex="-1">
          <h2>{{ t('suppliers.documents') }}</h2>
          <SupplierDocuments :supplier-id="id" :can-write="canWrite" />
        </section>

        <section id="supplier-changes" class="detail-section" tabindex="-1">
          <h2>{{ t('suppliers.changes') }}</h2>
          <p v-if="!changes.length" class="empty">{{ t('suppliers.noRecords') }}</p>
          <el-timeline v-else class="history">
            <el-timeline-item v-for="change in changes" :key="change.id" :timestamp="formatTime(change.createdAt)">
              <strong>{{ changeSummaryLabel(change, 'supplier', t) }}</strong>
              <p>{{ operatorDisplayName(change.operatorName, t) || '—' }} · {{ changeSectionLabel(change.section, t) }}</p>
            </el-timeline-item>
          </el-timeline>
        </section>
      </div>
    </template>

    <el-dialog v-model="contactOpen" :title="t('suppliers.addContact')" width="min(520px, 94vw)">
      <el-form size="small" :model="contact" label-position="top" @submit.prevent="saveContact">
        <el-form-item :label="t('suppliers.name')" required><el-input v-model="contact.name" /></el-form-item>
        <el-form-item :label="t('suppliers.department')"><el-input v-model="contact.department" /></el-form-item>
        <el-form-item :label="t('suppliers.titleField')"><el-input v-model="contact.title" /></el-form-item>
        <el-form-item :label="t('suppliers.phone')"><el-input v-model="contact.phone" /></el-form-item>
        <el-form-item :label="t('suppliers.email')"><el-input v-model="contact.email" /></el-form-item>
        <el-form-item><el-checkbox v-model="contact.isPrimary">{{ t('suppliers.primary') }}</el-checkbox></el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="savingRelation" @click="contactOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="savingRelation" @click="saveContact">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="ownerOpen" :title="t('suppliers.addOwner')" width="min(520px, 94vw)">
      <el-form size="small" :model="owner" label-position="top" @submit.prevent="saveOwner">
        <el-form-item :label="t('suppliers.employee')" required>
          <el-select v-model="owner.employeeId" filterable @change="employeeChanged">
            <el-option v-for="person in employees" :key="person.id" :label="[person.employeeNo, person.name].filter(Boolean).join(' · ')" :value="person.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('suppliers.responsibility')">
          <el-select v-model="owner.responsibilityCode">
            <el-option v-for="option in ownerOptions" :key="option.code" :label="option.label" :value="option.code" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('suppliers.period')">
          <el-date-picker v-model="owner.dates" type="daterange" value-format="YYYY-MM-DD" />
        </el-form-item>
        <el-form-item><el-checkbox v-model="owner.isPrimary">{{ t('suppliers.primary') }}</el-checkbox></el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="savingRelation" @click="ownerOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="savingRelation" @click="saveOwner">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { del, get, post, put, quietErrors, type Envelope } from '../api'
import { countryName } from '../lib/countries'
import { businessRoleLabel, changeSectionLabel, changeSummaryLabel, operatorDisplayName } from '../lib/masterDataDisplay'
import { supplierDetailLookups, type LookupOption, type OwnerCandidate } from '../lib/supplierDetailLookups'
import { supplierProfile, type SupplierProfile } from '../lib/supplierProfile'
import { useAuthStore } from '../stores/auth'
import CreditRating from '../components/CreditRating.vue'
import SupplierDocuments from '../components/SupplierDocuments.vue'
import SupplierProfileFields from '../components/SupplierProfileFields.vue'

interface Supplier extends SupplierProfile {
  id: string
  code: string
  name: string
  country: string
  status: string
  contactName?: string
  contactPhone?: string
  contactEmail?: string
  creditGrade?: string
  creditGradedAt?: string
}
interface ContactRow { id: string; name: string; email: string; phone: string; department: string; title: string; isPrimary: boolean }
interface OwnerRow { id: string; employeeName: string; responsibilityCode: string; startDate: string; endDate: string; isPrimary: boolean }
interface ChangeRow { id: string; summary: string; operatorName: string; section: string; createdAt: string }

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const id = String(route.params.id)
const endpoint = '/suppliers/' + id
const supplier = ref<Supplier>()
const loading = ref(true)
const canMaintain = ref(false)
const canWrite = computed(() => canMaintain.value && auth.can('masterdata:supplier:write'))
const canManageOwners = ref(false)
const canEditOwners = computed(() => canWrite.value && canManageOwners.value)
const contacts = ref<ContactRow[]>([])
const owners = ref<OwnerRow[]>([])
const changes = ref<ChangeRow[]>([])
const employees = ref<(OwnerCandidate & { employeeNo?: string })[]>([])
const businessOptions = ref<LookupOption[]>([])
const paymentOptions = ref<LookupOption[]>([])
const ownerOptions = ref<LookupOption[]>([])
const edit = ref(supplierProfile())
const baseline = ref(supplierProfile())
const profileError = ref('')
const savingProfile = ref(false)
const dirty = computed(() => JSON.stringify(supplierProfile(edit.value)) !== JSON.stringify(baseline.value))
const contactOpen = ref(false)
const ownerOpen = ref(false)
const savingRelation = ref(false)
const newContact = () => ({ name: '', department: '', title: '', phone: '', email: '', isPrimary: false, remark: '' })
const newOwner = () => ({ employeeId: '', employeeName: '', responsibilityCode: 'PROCUREMENT', isPrimary: false, dates: [] as string[] })
const contact = reactive(newContact())
const owner = reactive(newOwner())

const fullName = computed(() => locale.value === 'zh'
  ? supplier.value?.nameZh || supplier.value?.nameEn || supplier.value?.name
  : supplier.value?.nameEn || supplier.value?.nameZh || supplier.value?.name)
const displayName = computed(() => supplier.value?.shortName?.trim() || fullName.value)
const sections = computed(() => [
  { key: 'profile', label: t('suppliers.profile') },
  { key: 'contacts', label: t('suppliers.contacts') },
  { key: 'owners', label: t('suppliers.owners') },
  { key: 'credit', label: t('credit.tab') },
  { key: 'documents', label: t('suppliers.documents') },
  { key: 'changes', label: t('suppliers.changes') },
])
const activeSection = ref('profile')
const readFields = computed(() => {
  const value = supplier.value
  if (!value) return []
  return [
    { label: t('suppliers.nameZh'), value: value.nameZh },
    { label: t('suppliers.nameEn'), value: value.nameEn },
    { label: t('suppliers.shortName'), value: value.shortName },
    { label: t('suppliers.country'), value: value.countryCode ? countryName(value.countryCode, locale.value) : '' },
    { label: t('suppliers.taxId'), value: value.taxId },
    { label: t('suppliers.businessType'), value: (value.businessTypes || []).map(optionLabel).join(' · ') },
    { label: t('suppliers.currency'), value: value.currency },
    { label: t('suppliers.paymentTerm'), value: value.paymentTerm },
    { label: t('suppliers.registeredAddress'), value: value.registeredAddress, wide: true },
    { label: t('suppliers.address'), value: value.address, wide: true },
    { label: t('suppliers.remark'), value: value.remark, wide: true },
  ]
})

function optionLabel(code: string) {
  return businessRoleLabel(code, t, businessOptions.value.find(option => option.code === code)?.label || code)
}

function backToSuppliers() {
  const back = String(window.history.state?.back || '')
  if (back === '/basic/suppliers' || back.startsWith('/basic/suppliers?')) router.back()
  else void router.push('/basic/suppliers')
}

async function jump(section: unknown, behavior: ScrollBehavior = 'smooth') {
  if (typeof section !== 'string' || !sections.value.some(item => item.key === section)) return
  activeSection.value = section
  await nextTick()
  const target = document.getElementById('supplier-' + section)
  target?.scrollIntoView({ block: 'start', behavior })
  target?.focus({ preventScroll: true })
}

function selectSection(section: string) {
  if (route.query.tab === section) void jump(section)
  else void router.replace({ query: { ...route.query, tab: section } })
}
watch(() => route.query.tab, section => void jump(section))

function formatTime(value: string) {
  return value ? new Date(value).toLocaleString(locale.value) : ''
}

function acceptProfile(value: Supplier) {
  supplier.value = value
  baseline.value = supplierProfile(value)
  cancelProfile()
}

function cancelProfile() {
  edit.value = supplierProfile(baseline.value)
  profileError.value = ''
}

async function saveProfile() {
  if (!canWrite.value || !supplier.value || !dirty.value || savingProfile.value) return
  const value = supplierProfile(edit.value)
  if (!value.nameZh.trim() && !value.nameEn.trim()) {
    profileError.value = t('suppliers.nameRequired')
    return
  }
  const current = supplier.value
  const { supplier: updated } = await persistProfile(value, current)
  if (updated) {
    acceptProfile(updated)
    ElMessage.success(t('suppliers.saved'))
    await Promise.allSettled([loadChanges()])
  }
}

async function persistProfile(value: SupplierProfile, current: Supplier) {
  savingProfile.value = true
  profileError.value = ''
  try {
    return await put<{ supplier: Supplier }>(endpoint, {
      ...value,
      name: value.nameZh || value.nameEn,
      country: value.countryCode === current.countryCode ? current.country : value.countryCode,
      contactName: current.contactName || '',
      contactPhone: current.contactPhone || '',
      contactEmail: current.contactEmail || '',
    }, quietErrors)
  } catch (error) {
    profileError.value = (error as Partial<Envelope<unknown>> | undefined)?.message || t('common.requestFailed')
    return { supplier: undefined }
  } finally {
    savingProfile.value = false
  }
}

async function loadContacts() {
  contacts.value = (await get<{ contacts?: ContactRow[] }>(endpoint + '/contacts')).contacts || []
}

async function loadOwners() {
  owners.value = (await get<{ owners?: OwnerRow[] }>(endpoint + '/owners')).owners || []
}

async function loadChanges() {
  changes.value = (await get<{ changes?: ChangeRow[] }>(endpoint + '/changes')).changes || []
}

async function refreshRelations(load: () => Promise<void>) {
  await Promise.allSettled([load(), loadChanges()])
}

function openContact() {
  if (!canWrite.value) return
  Object.assign(contact, newContact())
  contactOpen.value = true
}

async function saveContact() {
  if (!canWrite.value || savingRelation.value) return
  savingRelation.value = true
  try {
    await post(endpoint + '/contacts', { contact })
    contactOpen.value = false
    await refreshRelations(loadContacts)
  } finally {
    savingRelation.value = false
  }
}

async function removeContact(row: ContactRow) {
  if (!canWrite.value) return
  try { await ElMessageBox.confirm(t('suppliers.confirmDeactivate')) } catch { return }
  await del(endpoint + '/contacts/' + row.id)
  await refreshRelations(loadContacts)
}

function openOwner() {
  if (!canEditOwners.value) return
  Object.assign(owner, newOwner())
  ownerOpen.value = true
}

function employeeChanged(value: string) {
  owner.employeeName = employees.value.find(employee => String(employee.id) === String(value))?.name || ''
}

async function saveOwner() {
  if (!canEditOwners.value || savingRelation.value) return
  const { dates, ...fields } = owner
  savingRelation.value = true
  try {
    await post(endpoint + '/owners', { owner: { ...fields, startDate: dates?.[0] || '', endDate: dates?.[1] || '' } })
    ownerOpen.value = false
    await refreshRelations(loadOwners)
  } finally {
    savingRelation.value = false
  }
}

async function removeOwner(row: OwnerRow) {
  if (!canEditOwners.value) return
  try { await ElMessageBox.confirm(t('suppliers.confirmDeactivate')) } catch { return }
  await del(endpoint + '/owners/' + row.id)
  await refreshRelations(loadOwners)
}

function onRated(grade: string) {
  if (!supplier.value) return
  supplier.value.creditGrade = grade
  supplier.value.creditGradedAt = new Date().toISOString()
}

onMounted(async () => {
  try {
    const detail = await get<{ supplier: Supplier; canMaintain?: boolean }>(endpoint)
    canMaintain.value = detail.canMaintain !== false
    acceptProfile(detail.supplier)
    const lookups = supplierDetailLookups(get)
    void lookups.businessOptions.then(value => { businessOptions.value = value })
    void lookups.paymentOptions.then(value => { paymentOptions.value = value })
    void lookups.ownerOptions.then(value => { ownerOptions.value = value })
    void lookups.canManageOwners.then(value => { canManageOwners.value = value })
    void lookups.employees.then(value => { employees.value = value })
    await Promise.allSettled([loadContacts(), loadOwners(), loadChanges()])
    await jump(route.query.tab, 'instant')
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.supplier-detail { max-width: 1500px; margin: 0 auto; }
.hero { display: flex; align-items: flex-start; justify-content: space-between; flex-wrap: wrap; padding: 14px 18px; gap: 10px 20px; color: #fff; background: linear-gradient(120deg, #143f5a, #176b87); border-radius: 10px; }
.hero-main { min-width: 0; }
.hero h1 { margin: 0; font-size: 20px; overflow-wrap: anywhere; }
.hero h1 small { margin-inline-start: 6px; color: #e5f2f7; font-size: 12px; font-weight: 400; }
.hero-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 10px; margin-top: 7px; color: #e5f2f7; font-size: 13px; }
.hero-back { flex: none; }
.section-nav { position: sticky; top: 0; z-index: 2; display: flex; flex-wrap: wrap; gap: 2px 8px; margin-block: 8px; padding: 4px 8px; border: 1px solid #dce7f4; border-radius: 9px; background: #fff; }
.section-nav button { padding: 6px 9px; font: inherit; font-size: 13px; color: #205573; background: transparent; border: 0; border-radius: 5px; cursor: pointer; }
.section-nav button:hover, .section-nav button:focus-visible, .section-nav [aria-current='location'] { background: #edf6fb; }
.section-nav [aria-current='location'] { font-weight: 600; }
.detail-sheet { padding-inline: 16px; border: 1px solid #e4ebf3; border-radius: 9px; background: #fff; }
.detail-section { padding-block: 12px; font-size: 13px; scroll-margin-top: 82px; }
.detail-section:focus { outline: none; }
.detail-section + .detail-section { border-top: 1px solid #e4ebf3; }
.detail-section h2 { margin: 0 0 8px; color: #173b52; font-size: 14px; }
.detail-section h2 small { margin-inline-start: 4px; color: var(--el-text-color-secondary); font-weight: 400; }
.section-head { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; }
.profile-actions { display: flex; flex-wrap: wrap; justify-content: flex-end; align-items: center; gap: 8px; }
.profile-actions .el-button + .el-button { margin-left: 0; }
.error-text { margin-inline-end: auto; color: var(--el-color-danger-dark-2); font-size: 12px; }
.profile-read { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px 16px; margin: 0; }
.profile-read dt { margin-bottom: 3px; color: var(--el-text-color-secondary); font-size: 12px; }
.profile-read dd { margin: 0; white-space: pre-wrap; overflow-wrap: anywhere; }
.full-line { grid-column: 1 / -1; }
.record-list, .record-row + .record-row { border-top: 1px solid #edf0f3; }
.record-row { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.5fr) minmax(0, 1fr) max-content; align-items: start; gap: 4px 14px; padding-block: 7px; line-height: 20px; overflow-wrap: anywhere; }
.record-name, .record-detail { display: flex; flex-wrap: wrap; align-items: baseline; gap: 3px 8px; min-width: 0; }
.record-meta, .empty, .history p { color: var(--el-text-color-secondary); font-size: 12px; }
.record-action { justify-self: end; }
.empty { margin: 0; }
.history { margin: 0; padding: 4px 0 0 6px; }
.history p { margin: 3px 0 0; }
@media (max-width: 760px) { .profile-read { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 540px) {
  .profile-read { grid-template-columns: minmax(0, 1fr); }
  .detail-section { scroll-margin-top: 112px; }
  .record-row { grid-template-columns: minmax(0, 1fr) auto; gap: 3px 8px; }
  .record-row > div { grid-column: 1; }
  .record-action { grid-column: 2; grid-row: 1; }
}
</style>
