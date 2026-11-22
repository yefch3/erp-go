<template>
  <el-dialog :model-value="open" :title="t('suppliers.create')" width="min(1100px, 94vw)" top="4vh" append-to-body :close-on-click-modal="false" :close-on-press-escape="!saving" :before-close="close">
    <div ref="body" class="create-body">
      <nav class="create-nav" :aria-label="t('suppliers.create')">
        <button v-for="section in sections" :key="section.id" type="button" @click="jump(section.id)">{{ section.label }}</button>
      </nav>
        <section id="new-supplier-profile" class="create-section">
          <h3>{{ t('suppliers.profile') }}</h3>
          <p class="hint">{{ t('suppliers.codeAutoNotice') }}</p>
          <el-form :disabled="saving || !!supplierId" label-position="top" size="small" @submit.prevent>
            <SupplierProfileFields v-model="profile" :business-options="businessOptions" :payments="paymentOptions" :role-label="roleLabel" />
          </el-form>
        </section>

      <el-form label-position="top" size="small" :disabled="saving" @submit.prevent="save">
        <section id="new-supplier-contacts" class="create-section">
          <div class="section-heading"><h3>{{ t('suppliers.contacts') }}</h3><el-button link type="primary" @click="addContact">{{ t('suppliers.addContact') }}</el-button></div>
          <p v-if="!contacts.length" class="hint">{{ t('suppliers.noRecords') }}</p>
          <fieldset v-for="(row, index) in contacts" :key="row.key" :disabled="row.saved">
            <legend>{{ t('suppliers.contacts') }} {{ index + 1 }}</legend>
            <div class="create-grid">
              <el-form-item v-for="field in contactFields" :key="field.key" :label="t(field.label)" :required="field.key === 'name'">
                <el-input v-model="row.data[field.key]" :disabled="row.saved" />
              </el-form-item>
              <div class="row-controls">
                <el-checkbox :model-value="row.data.isPrimary" :disabled="row.saved" @change="setPrimary(contacts, row, $event === true)">{{ t('suppliers.primary') }}</el-checkbox>
                <el-button v-if="!row.saved" link type="danger" @click="contacts.splice(index, 1)">{{ t('suppliers.removeEntry') }}</el-button>
                <span v-else class="hint">{{ t('suppliers.saved') }}</span>
              </div>
            </div>
          </fieldset>
        </section>

        <section id="new-supplier-owners" class="create-section">
          <div class="section-heading">
            <h3>{{ t('suppliers.owners') }}</h3>
            <el-button v-if="canManageOwners" link type="primary" :loading="loadingEmployees" @click="addOwner">{{ t('suppliers.addOwner') }}</el-button>
          </div>
          <p class="hint">{{ t('suppliers.defaultOwnerHint') }}</p>
          <fieldset v-for="(row, index) in owners" :key="row.key" :disabled="row.saved">
            <legend>{{ t('suppliers.owners') }} {{ index + 1 }}</legend>
            <div class="create-grid">
              <el-form-item :label="t('suppliers.employee')" required>
                <el-select v-model="row.data.employeeId" :disabled="row.saved" filterable @change="row.data.employeeName = employees.find(person => person.id === row.data.employeeId)?.name || ''">
                  <el-option v-for="person in employees" :key="person.id" :value="person.id" :label="person.name" />
                </el-select>
              </el-form-item>
              <el-form-item :label="t('suppliers.responsibility')" required>
                <el-select v-model="row.data.responsibilityCode" :disabled="row.saved">
                  <el-option v-for="option in ownerOptions" :key="option.code" :value="option.code" :label="option.label" />
                </el-select>
              </el-form-item>
              <el-form-item :label="t('suppliers.period')">
                <el-date-picker v-model="row.data.dates" :disabled="row.saved" type="daterange" value-format="YYYY-MM-DD" />
              </el-form-item>
              <div class="row-controls">
                <el-checkbox :model-value="row.data.isPrimary" :disabled="row.saved" @change="setPrimary(owners, row, $event === true)">{{ t('suppliers.primary') }}</el-checkbox>
                <el-button v-if="!row.saved" link type="danger" @click="owners.splice(index, 1)">{{ t('suppliers.removeEntry') }}</el-button>
                <span v-else class="hint">{{ t('suppliers.saved') }}</span>
              </div>
            </div>
          </fieldset>
        </section>

        <section id="new-supplier-documents" class="create-section">
          <div class="section-heading">
            <h3>{{ t('suppliers.documents') }} <small v-if="files.length">{{ files.length }}</small></h3>
            <el-button type="primary" plain @click="picker?.click()">{{ t('suppliers.addAttachment') }}</el-button>
            <input ref="picker" type="file" multiple hidden @change="selectFiles" />
          </div>
          <p class="hint">{{ t('suppliers.attachmentHint') }}</p>
          <div v-for="(row, index) in files" :id="'new-supplier-file-' + row.key" :key="row.key" class="file-row" :class="{ recent: recentFiles.has(row.key) && !row.saved }">
            <span class="filename">{{ row.data.file.name }}</span>
            <label :for="'new-supplier-date-' + row.key">{{ t('suppliers.reminderDate') }}</label>
            <el-date-picker :id="'new-supplier-date-' + row.key" v-model="row.data.date" :disabled="row.saved" type="date" value-format="YYYY-MM-DD" />
            <span v-if="row.data.error" class="error-text">{{ row.data.error }}</span>
            <span v-if="row.saved" class="hint">{{ t('suppliers.saved') }}</span>
            <el-button v-else link type="danger" @click="files.splice(index, 1)">{{ t('suppliers.removeEntry') }}</el-button>
          </div>
        </section>
      </el-form>
    </div>
    <template #footer>
      <div class="create-footer">
        <span v-if="error" class="error-text" role="status">{{ error }}</span>
        <el-button :disabled="saving" @click="close(() => emit('update:open', false))">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ supplierId ? t('suppliers.retrySave') : t('common.save') }}</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, http, post, quietErrors, type Envelope } from '../api'
import { announceReminderChanged } from '../lib/homeReminders'
import { newIdempotencySession, withIdempotency } from '../lib/idempotency'
import { confirmPossibleDuplicates, type DuplicateCandidate } from '../lib/masterDataDuplicates'
import type { LookupOption, OwnerCandidate } from '../lib/supplierDetailLookups'
import { supplierProfile } from '../lib/supplierProfile'
import SupplierProfileFields from './SupplierProfileFields.vue'

const props = defineProps<{ open: boolean; canManageOwners: boolean; employeeId: string; businessOptions: LookupOption[]; paymentOptions: LookupOption[]; roleLabel: (code: string) => string }>()
const emit = defineEmits<{ 'update:open': [open: boolean]; created: [] }>()
const { t } = useI18n()
const sections = computed(() => [
  { id: 'profile', label: t('suppliers.profile') },
  { id: 'contacts', label: t('suppliers.contacts') },
  { id: 'owners', label: t('suppliers.owners') },
  { id: 'documents', label: t('suppliers.documents') },
])
const contactFields = [
  { key: 'name', label: 'suppliers.name' },
  { key: 'department', label: 'suppliers.department' },
  { key: 'title', label: 'suppliers.titleField' },
  { key: 'phone', label: 'suppliers.phone' },
  { key: 'email', label: 'suppliers.email' },
  { key: 'remark', label: 'suppliers.remark' },
] as const
let key = 0
function pending<T>(data: T) {
  return { data, saved: false, key: ++key, session: newIdempotencySession() }
}
type Pending<T> = ReturnType<typeof pending<T>>
const newContact = (isPrimary: boolean) => ({ name: '', department: '', title: '', phone: '', email: '', remark: '', isPrimary })
const newOwner = () => ({ employeeId: '', employeeName: '', responsibilityCode: 'PROCUREMENT', dates: [] as string[], isPrimary: false })
const contacts = ref<Pending<ReturnType<typeof newContact>>[]>([])
const owners = ref<Pending<ReturnType<typeof newOwner>>[]>([])
const files = ref<Pending<{ file: File; date: string; error: string }>[]>([])
const recentFiles = ref(new Set<number>())
const employees = ref<OwnerCandidate[]>([])
const ownerOptions = ref<LookupOption[]>([])
const loadingEmployees = ref(false)
const body = ref<HTMLElement>()
const picker = ref<HTMLInputElement>()
const saving = ref(false)
const error = ref('')
const supplierId = ref('')
const createSession = newIdempotencySession()
const profile = ref(supplierProfile())

watch(() => props.open, (open) => {
  if (!open) return
  profile.value = supplierProfile({ currency: 'USD', businessTypes: ['GENERAL'] })
  contacts.value = []
  owners.value = []
  files.value = []
  recentFiles.value = new Set()
  error.value = ''
  supplierId.value = ''
  createSession.reset()
  void nextTick(() => body.value?.scrollTo({ top: 0 }))
}, { immediate: true })

function jump(section: string) {
  body.value?.querySelector('#new-supplier-' + section)?.scrollIntoView({ block: 'start' })
}

function addContact() {
  contacts.value.push(pending(newContact(!contacts.value.length)))
}

async function addOwner() {
  if (!props.canManageOwners || loadingEmployees.value) return
  loadingEmployees.value = true
  try {
    if (!employees.value.length) {
      const [people, roles] = await Promise.all([
        get<{ employees?: OwnerCandidate[] }>('/employees', { page: 1, page_size: 200, status: 'ACTIVE' }),
        get<{ options?: LookupOption[] }>('/options', { category: 'SUPPLIER_OWNER_RESPONSIBILITY' }),
      ])
      employees.value = people.employees || []
      ownerOptions.value = roles.options || []
    }
    if (props.open) owners.value.push(pending(newOwner()))
  } catch {
    // The request interceptor shows lookup errors.
  } finally {
    loadingEmployees.value = false
  }
}

function setPrimary<T extends { isPrimary: boolean }>(rows: Pending<T>[], row: Pending<T>, enabled: boolean) {
  if (enabled) for (const other of rows) other.data.isPrimary = false
  row.data.isPrimary = enabled
}

async function selectFiles(event: Event) {
  const input = event.target as HTMLInputElement
  const selected = []
  for (const file of Array.from(input.files || [])) {
    if (!file.size || file.size > 10 * 1024 * 1024) {
      ElMessage.warning(file.name + ' · ' + t('suppliers.attachmentHint'))
      continue
    }
    selected.push(pending({ file, date: '', error: '' }))
  }
  input.value = ''
  if (!selected.length) return
  files.value.push(...selected)
  recentFiles.value = new Set(selected.map(row => row.key))
  await nextTick()
  body.value?.querySelector('#new-supplier-file-' + selected[0].key)?.scrollIntoView({ block: 'start' })
}

function validate() {
  if (!profile.value.nameZh.trim() && !profile.value.nameEn.trim()) {
    error.value = t('suppliers.nameRequired')
    jump('profile')
    return false
  }
  if (contacts.value.some(row => !row.data.name.trim())) {
    error.value = t('suppliers.contactRequired')
    jump('contacts')
    return false
  }
  const keys = new Set([props.employeeId + ':PROCUREMENT'])
  for (const { data } of owners.value) {
    const key = data.employeeId + ':' + data.responsibilityCode
    if (!data.employeeId || !data.responsibilityCode || keys.has(key)) {
      error.value = t('suppliers.ownerRequired')
      jump('owners')
      return false
    }
    keys.add(key)
  }
  return true
}

function message(error: unknown) {
  return (error as Partial<Envelope<unknown>> | undefined)?.message || t('common.requestFailed')
}

async function saveRows<T>(path: string, section: string, label: string, rows: Pending<T>[], payload: (data: T) => object) {
  for (const [index, row] of rows.entries()) {
    if (row.saved) continue
    try {
      await post(path, payload(row.data), withIdempotency(row.session, quietErrors))
      row.saved = true
    } catch (error) {
      jump(section)
      throw new Error(label + ' ' + (index + 1) + ' · ' + message(error))
    }
  }
}

async function upload(path: string) {
  let pendingFiles = 0
  for (const row of files.value) {
    if (row.saved) continue
    row.data.error = ''
    const { file, date } = row.data
    const form = new FormData()
    form.set('title', file.name)
    form.set('expiresOn', date || '')
    form.set('remindDays', '0')
    form.set('reminderEnabled', String(Boolean(date)))
    form.set('file', file)
    try {
      await http.post(path, form, withIdempotency(row.session, { ...quietErrors, timeout: 120000 }))
      row.saved = true
      announceReminderChanged()
    } catch (error) {
      row.data.error = message(error)
      pendingFiles++
    }
  }
  return pendingFiles
}

async function save() {
  if (saving.value || !validate()) return
  saving.value = true
  error.value = ''
  try {
    if (!supplierId.value) {
      const fields = supplierProfile(profile.value)
      const name = fields.nameZh.trim() || fields.nameEn.trim()
      const { candidates } = await get<{ candidates?: DuplicateCandidate[] }>('/suppliers/duplicates', { name, tax_id: fields.taxId })
      try { await confirmPossibleDuplicates(candidates || [], t) } catch { return }
      const { supplier } = await post<{ supplier: { id: string } }>('/suppliers', { ...fields, name, country: fields.countryCode }, withIdempotency(createSession, quietErrors))
      supplierId.value = supplier.id
    }
    const path = '/suppliers/' + supplierId.value
    await saveRows(path + '/contacts', 'contacts', t('suppliers.contacts'), contacts.value, contact => ({ contact }))
    if (owners.value.some(row => !row.saved)) {
      if (!props.canManageOwners) throw new Error(t('suppliers.ownerRequired'))
      await saveRows(path + '/owners', 'owners', t('suppliers.owners'), owners.value, ({ dates, ...owner }) => ({
        owner: { ...owner, startDate: dates?.[0] || '', endDate: dates?.[1] || '' },
      }))
    }
    const failures = await upload(path + '/documents')
    if (failures) {
      error.value = t('suppliers.attachmentRetry', { count: failures })
      jump('documents')
      return
    }
    emit('created')
    emit('update:open', false)
    ElMessage.success(t('suppliers.saved'))
  } catch (failure) {
    error.value = (supplierId.value ? t('suppliers.partialSave') + ' ' : '') + message(failure)
  } finally {
    saving.value = false
  }
}

async function close(done: () => void) {
  if (saving.value) return
  if (supplierId.value) {
    try { await ElMessageBox.confirm(t('suppliers.closePartial'), t('suppliers.create')) } catch { return }
    emit('created')
  }
  done()
}
</script>

<style scoped>
.create-body { max-height: calc(92dvh - 118px); overflow: auto; padding-inline: 2px 10px; }
.create-nav { display: flex; flex-wrap: wrap; gap: 3px 8px; position: sticky; top: 0; z-index: 2; padding: 5px 0; border-bottom: 1px solid #e4ebf3; background: #fff; }
.create-nav button { padding: 5px 8px; border: 0; border-radius: 4px; color: #205573; background: transparent; cursor: pointer; }
.create-nav button:hover, .create-nav button:focus-visible { background: #edf6fb; }
.create-section { padding: 12px 0 6px; scroll-margin-top: 72px; }
.create-section + .create-section { border-top: 1px solid #e4ebf3; }
.create-section h3 { margin: 0 0 8px; color: #173b52; font-size: 14px; }
.create-section h3 small { margin-inline-start: 5px; color: var(--el-text-color-secondary); }
.section-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; }
.create-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 0 16px; }
.create-grid :deep(.el-form-item) { min-width: 0; margin-bottom: 8px; }
.create-grid :deep(.el-select), .create-grid :deep(.el-date-editor) { width: 100%; }
fieldset { min-width: 0; margin: 0 0 8px; padding: 6px 10px; border: 1px solid #dce7f4; border-radius: 6px; }
legend { padding-inline: 5px; color: #205573; font-size: 12px; }
.row-controls, .file-row, .create-footer { display: flex; flex-wrap: wrap; align-items: center; gap: 6px 10px; }
.file-row { padding: 4px 8px; margin-block: 4px; border-radius: 5px; scroll-margin-top: 72px; font-size: 13px; }
.file-row.recent { background: #edf6fb; }
.filename { min-width: min(200px, 100%); overflow-wrap: anywhere; }
.file-row label, .hint { color: var(--el-text-color-secondary); font-size: 12px; }
.hint { margin: 0 0 8px; }
.error-text { color: var(--el-color-danger-dark-2); font-size: 12px; }
.create-footer { justify-content: flex-end; }
.create-footer .error-text { margin-inline-end: auto; }
.create-footer .el-button + .el-button { margin-left: 0; }
@media (max-width: 780px) { .create-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 520px) { .create-grid { grid-template-columns: minmax(0, 1fr); } }
</style>
