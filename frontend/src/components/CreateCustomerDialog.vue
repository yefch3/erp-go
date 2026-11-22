<template>
  <el-dialog
    :model-value="open" title="新建客户" class="create-customer-dialog" width="min(1120px, 94vw)" top="4vh"
    append-to-body :close-on-click-modal="false" :close-on-press-escape="!saving" :before-close="confirmClose"
  >
    <div ref="scrollArea" class="create-scroll">
      <nav class="create-nav" aria-label="新建客户分区">
        <button v-for="section in sections" :key="section.id" type="button" @click="jump(section.id)">{{ section.title }}</button>
        <span>带 * 的项目为必填项</span>
      </nav>
      <el-form label-position="top" size="small" :disabled="saving" @submit.prevent>
        <section id="new-customer-profile" class="create-section">
          <h3>客户资料</h3>
          <div class="fields">
            <el-form-item label="客户名称" required><el-input v-model="profile.name" :disabled="locked" maxlength="200" /></el-form-item>
            <el-form-item label="客户简称"><el-input v-model="profile.shortName" :disabled="locked" maxlength="100" /></el-form-item>
            <el-form-item label="客户编码"><el-input v-model="profile.code" :disabled="locked" maxlength="50" placeholder="留空自动生成" /></el-form-item>
            <el-form-item label="英文名称"><el-input v-model="profile.englishName" :disabled="locked" /></el-form-item>
            <el-form-item label="国家 / 地区">
              <el-select v-model="profile.countryCode" :disabled="locked" filterable clearable>
                <el-option v-for="country in countries" :key="country.code" :label="country.name" :value="country.code" />
              </el-select>
            </el-form-item>
            <el-form-item label="所在时区">
              <el-select v-model="profile.timezone" :disabled="locked" filterable allow-create clearable placeholder="选择或输入 IANA 时区">
                <el-option v-for="zone in timezones" :key="zone" :label="zone" :value="zone" />
              </el-select>
            </el-form-item>
            <el-form-item label="客户类型">
              <el-select v-model="profile.customerType" :disabled="locked" clearable>
                <el-option v-for="item in options.customerTypes" :key="item.code" :label="item.label" :value="item.code" />
              </el-select>
            </el-form-item>
            <el-form-item label="客户来源">
              <el-select v-model="profile.source" :disabled="locked" clearable>
                <el-option v-for="item in options.sources" :key="item.code" :label="item.label" :value="item.code" />
              </el-select>
            </el-form-item>
            <el-form-item label="业务状态">
              <el-select v-model="profile.businessStatus" :disabled="locked">
                <el-option label="潜在" value="PROSPECT" /><el-option label="合作中" value="COOPERATING" />
                <el-option label="暂停合作" value="PAUSED" /><el-option label="已停用" value="INACTIVE" />
              </el-select>
            </el-form-item>
            <el-form-item v-for="field in profileFields" :key="field.key" :label="field.label">
              <el-input v-model="profile[field.key]" :disabled="locked" :placeholder="'placeholder' in field ? field.placeholder : undefined" />
            </el-form-item>
            <el-form-item label="标签">
              <el-select v-model="profile.tags" :disabled="locked" multiple filterable allow-create default-first-option placeholder="输入后按回车添加" />
            </el-form-item>
            <el-form-item v-for="field in companyFields" :key="field.key" :label="field.label">
              <el-input v-model="company[field.key]" :disabled="basicSaved" :maxlength="field.max" />
            </el-form-item>
            <el-form-item class="full" label="备注"><el-input v-model="profile.remark" :disabled="locked" type="textarea" :rows="2" /></el-form-item>
          </div>
        </section>

        <section id="new-customer-address" class="create-section">
          <div class="section-heading"><h3>地址与税务</h3><el-button link type="primary" @click="addAddress">添加地址</el-button></div>
          <p v-if="!addresses.length" class="muted">可以添加办公、注册、收货和账单地址。</p>
          <fieldset v-for="(row, index) in addresses" :key="row.key" :disabled="row.saved">
            <legend>地址 {{ index + 1 }}</legend>
            <div class="fields">
              <el-form-item label="地址类型">
                <el-select v-model="row.data.addressType" :disabled="row.saved" @change="row.data.isDefault = false">
                  <el-option v-for="item in addressTypes" :key="item.value" :label="item.label" :value="item.value" />
                </el-select>
              </el-form-item>
              <el-form-item label="国家 / 地区">
                <el-select v-model="row.data.countryCode" :disabled="row.saved" filterable clearable>
                  <el-option v-for="country in countries" :key="country.code" :label="country.name" :value="country.code" />
                </el-select>
              </el-form-item>
              <el-form-item label="省 / 州"><el-input v-model="row.data.state" /></el-form-item>
              <el-form-item label="城市"><el-input v-model="row.data.city" /></el-form-item>
              <el-form-item label="邮编"><el-input v-model="row.data.postalCode" /></el-form-item>
              <div class="row-actions">
                <el-checkbox :model-value="row.data.isDefault" :disabled="row.saved" @change="setDefaultAddress(row, $event === true)">同类型默认地址</el-checkbox>
                <el-button v-if="!row.saved" link type="danger" @click="addresses.splice(index, 1)">移除</el-button>
                <span v-else class="saved-mark">已保存</span>
              </div>
              <el-form-item label="详细地址" required class="full"><el-input v-model="row.data.addressLine" maxlength="500" /></el-form-item>
            </div>
          </fieldset>
          <div class="fields">
            <el-form-item v-for="field in taxFields" :key="field.key" :label="field.label">
              <el-input v-model="profile[field.key]" :disabled="locked" />
            </el-form-item>
          </div>
        </section>

        <section id="new-customer-contacts" class="create-section">
          <div class="section-heading"><h3>联系人</h3><el-button link type="primary" @click="addContact">添加联系人</el-button></div>
          <p v-if="!contacts.length" class="muted">可录入多位联系人，并指定主要联系人。</p>
          <fieldset v-for="(row, index) in contacts" :key="row.key" :disabled="row.saved">
            <legend>联系人 {{ index + 1 }}</legend>
            <div class="fields">
              <el-form-item v-for="field in contactFields" :key="field.key" :label="field.label">
                <div v-if="field.key === 'phone'" class="phone-field">
                  <el-select v-model="row.data.phoneDial" :disabled="row.saved" aria-label="电话区号" filterable clearable placeholder="区号">
                    <el-option v-for="item in DIAL_CODES" :key="item.dial" :label="item.dial" :value="item.dial">
                      {{ item.dial }} · {{ item.countries }}
                    </el-option>
                  </el-select>
                  <el-input v-model="row.data.phone" :disabled="row.saved" placeholder="电话号码" />
                </div>
                <el-input v-else v-model="row.data[field.key]" :placeholder="'placeholder' in field ? field.placeholder : undefined" :disabled="row.saved" />
              </el-form-item>
              <el-form-item label="邮件接收">
                <el-select v-model="row.data.emailPermission" :disabled="row.saved">
                  <el-option label="允许接收" value="ALLOWED" />
                  <el-option label="已退订" value="OPTED_OUT" />
                  <el-option label="邮箱无效" value="INVALID" />
                </el-select>
              </el-form-item>
              <el-form-item label="邮件类型">
                <el-select v-model="row.data.emailCategories" :disabled="row.saved" multiple placeholder="不选表示全部类型">
                  <el-option label="业务通知" value="BUSINESS" /><el-option label="报价 / 产品" value="QUOTATION" />
                  <el-option label="船期 / 物流" value="SHIPPING" /><el-option label="营销活动" value="MARKETING" />
                </el-select>
              </el-form-item>
              <div class="row-actions">
                <el-checkbox :model-value="row.data.isPrimary" :disabled="row.saved" @change="setPrimary(contacts, row, $event === true)">主要联系人</el-checkbox>
                <el-button v-if="!row.saved" link type="danger" @click="contacts.splice(index, 1)">移除</el-button>
                <span v-else class="saved-mark">已保存</span>
              </div>
              <el-form-item class="full" label="联系人备注"><el-input v-model="row.data.remark" :disabled="row.saved" /></el-form-item>
            </div>
          </fieldset>
        </section>

        <section id="new-customer-owners" class="create-section">
          <div class="section-heading">
            <h3>负责人</h3>
            <el-button v-if="canChooseEmployees" link type="primary" :loading="loadingEmployees" @click="addOwner">添加负责人</el-button>
          </div>
          <p class="muted">系统默认将当前用户设为主要销售负责人。可为其他员工指定职责。</p>
          <fieldset v-for="(row, index) in owners" :key="row.key" :disabled="row.saved">
            <legend>负责人 {{ index + 1 }}</legend>
            <div class="fields">
              <el-form-item label="员工" required>
                <el-select v-model="row.data.employeeId" :disabled="row.saved" filterable>
                  <el-option v-for="person in employees" :key="person.id" :value="String(person.id)" :label="person.name + (person.departmentName ? ' · ' + person.departmentName : '')" />
                </el-select>
              </el-form-item>
              <el-form-item label="职责" required>
                <el-select v-model="row.data.responsibilityCode" :disabled="row.saved">
                  <el-option v-for="item in responsibilityOptions" :key="item.code" :value="item.code" :label="item.label" />
                </el-select>
              </el-form-item>
              <el-form-item label="生效日期">
                <el-date-picker v-model="row.data.startDate" :disabled="row.saved" value-format="YYYY-MM-DD" />
              </el-form-item>
              <el-form-item label="结束日期">
                <el-date-picker v-model="row.data.endDate" :disabled="row.saved" value-format="YYYY-MM-DD" />
              </el-form-item>
              <div class="row-actions">
                <el-checkbox :model-value="row.data.isPrimary" :disabled="row.saved" @change="setPrimary(owners, row, $event === true)">主要负责人</el-checkbox>
                <el-button v-if="!row.saved" link type="danger" @click="owners.splice(index, 1)">移除</el-button>
                <span v-else class="saved-mark">已保存</span>
              </div>
            </div>
          </fieldset>
        </section>

        <section id="new-customer-credit" class="create-section">
          <h3>结算信用</h3>
          <div class="fields">
            <el-form-item label="结算币种">
              <el-select v-model="profile.currency" :disabled="locked">
                <el-option v-for="currency in CURRENCIES" :key="currency" :label="currency" :value="currency" />
              </el-select>
            </el-form-item>
            <el-form-item label="付款方式">
              <el-select v-model="profile.paymentTerm" :disabled="locked" filterable clearable>
                <el-option v-for="item in options.paymentMethods" :key="item.code" :value="item.code" :label="item.label" />
              </el-select>
            </el-form-item>
            <el-form-item label="客户等级">
              <el-select v-model="company.creditGrade" :disabled="basicSaved" clearable>
                <el-option v-for="grade in ['A', 'B', 'C', 'D']" :key="grade" :value="grade" />
              </el-select>
            </el-form-item>
            <el-form-item label="信用额度"><el-input-number v-model="creditAmount" :disabled="locked" :min="0" :precision="2" :controls="false" /></el-form-item>
            <el-form-item label="额度币种">
              <el-select v-model="profile.creditCurrency" :disabled="locked" clearable :placeholder="profile.currency">
                <el-option v-for="currency in CURRENCIES" :key="currency" :label="currency" :value="currency" />
              </el-select>
            </el-form-item>
            <el-form-item label="信用状态">
              <el-select v-model="profile.creditStatus" :disabled="locked">
                <el-option label="正常" value="NORMAL" /><el-option label="关注" value="WATCH" />
                <el-option label="暂停赊销" value="CREDIT_SUSPENDED" />
              </el-select>
            </el-form-item>
          </div>
        </section>

        <section id="new-customer-documents" class="create-section">
          <div class="section-heading">
            <h3>客户附件 <span v-if="files.length" class="attachment-count">{{ files.length }} 个文件</span></h3>
            <el-button plain type="primary" @click="fileInput?.click()">添加附件</el-button>
            <input ref="fileInput" type="file" multiple hidden @change="selectFiles" />
          </div>
          <p v-if="!files.length" class="muted">添加附件后，可为每个文件设置提醒日期。单个文件最大 10 MB。</p>
          <p v-else class="muted">选填提醒日期，到时会在「我的待办」提醒。</p>
          <div
            v-for="(row, index) in files"
            :id="'new-customer-file-' + row.key"
            :key="row.key"
            class="upload-row"
            :class="{ 'upload-row--new': recentFileKeys.has(row.key) && !row.saved }"
          >
            <span class="filename">{{ row.data.file.name }}</span>
            <div class="attachment-reminder">
              <label :for="'customer-attachment-date-' + row.key">提醒日期（选填）</label>
              <el-date-picker
                :id="'customer-attachment-date-' + row.key"
                v-model="row.data.reminderDate"
                :disabled="row.saved"
                placeholder="选择日期"
                value-format="YYYY-MM-DD"
              />
            </div>
            <span v-if="row.data.error" class="error-text">{{ row.data.error }}</span>
            <span v-if="row.saved" class="saved-mark">已上传</span>
            <el-button v-else link type="danger" @click="files.splice(index, 1)">移除</el-button>
          </div>
        </section>
      </el-form>
    </div>
    <template #footer>
      <div class="create-footer">
        <p class="error-text" role="status">{{ saveError }}</p>
        <el-button :disabled="saving" @click="confirmClose(() => emit('update:open', false))">{{ locked ? '关闭' : '取消' }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ submitLabel }}</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, reactive, ref, shallowRef, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, http, post, put, quietErrors } from '../api'
import { CURRENCIES, DIAL_CODES, dialCodeOfCode } from '../constants'
import { countryName, countryOptions } from '../lib/countries'
import { customerAddressInput, customerContactInput, validateCustomerContact, validateCustomerProfile } from '../lib/customerForms'
import { announceReminderChanged } from '../lib/homeReminders'
import { newIdempotencySession, withIdempotency } from '../lib/idempotency'
import { confirmPossibleDuplicates, type DuplicateCandidate } from '../lib/masterDataDuplicates'
import { portTimezoneOptions } from '../lib/portOptions'
import { useAuthStore } from '../stores/auth'

const props = defineProps<{ open: boolean; canManageOwners: boolean }>()
const emit = defineEmits<{ 'update:open': [open: boolean]; created: [] }>()
const { t, locale } = useI18n()
const auth = useAuthStore()

const sections = [
  { id: 'profile', title: '客户资料' }, { id: 'address', title: '地址与税务' },
  { id: 'contacts', title: '联系人' }, { id: 'owners', title: '负责人' },
  { id: 'credit', title: '结算信用' }, { id: 'documents', title: '附件' },
]
const profileFields = [
  { key: 'website', label: '官网', placeholder: 'https://example.com' },
  { key: 'industry', label: '所属行业' },
  { key: 'primaryLanguage', label: '主要语言' },
] as const
const companyFields = [
  { key: 'archiveCreator', label: '建档人', max: 100 },
  { key: 'companyPhone', label: '公司电话', max: 50 },
  { key: 'faxNumber', label: '传真号码', max: 50 },
  { key: 'companyEmail', label: '公司邮箱', max: 200 },
] as const
const taxFields = [
  { key: 'registeredName', label: '注册名称' },
  { key: 'registrationNo', label: '注册编号' },
  { key: 'taxId', label: '税务识别号' },
] as const
const contactFields = [
  { key: 'name', label: '姓名' }, { key: 'email', label: '邮箱' },
  { key: 'phone', label: '电话' }, { key: 'mobile', label: '手机' },
  { key: 'department', label: '部门' }, { key: 'title', label: '职位' },
  { key: 'instantMessaging', label: '即时通讯', placeholder: 'WhatsApp / 微信等' },
  { key: 'language', label: '首选语言' },
] as const
const addressTypes = [
  { value: 'OFFICE', label: '办公地址' }, { value: 'REGISTERED', label: '注册地址' },
  { value: 'BILLING', label: '账单地址' }, { value: 'SHIPPING', label: '收货地址' },
]

const emptyProfile = () => ({
  code: '', name: '', shortName: '', englishName: '', countryCode: '', timezone: '',
  businessStatus: 'PROSPECT', customerType: '', source: '', industry: '', website: '',
  primaryLanguage: '', tags: [] as string[], remark: '', registeredName: '', registrationNo: '',
  taxId: '', currency: 'USD',
  paymentTerm: '', creditCurrency: '', creditStatus: 'NORMAL',
})
const emptyCompany = () => ({ archiveCreator: '', companyPhone: '', faxNumber: '', companyEmail: '', creditGrade: '' })
const profile = reactive(emptyProfile())
const company = reactive(emptyCompany())
const creditAmount = ref<number>()
const newAddress = (isDefault: boolean) => ({
  addressType: 'OFFICE', countryCode: profile.countryCode, state: '', city: '',
  postalCode: '', addressLine: '', isDefault,
})
const newContact = (isPrimary: boolean) => ({
  name: '', email: '', phone: '', phoneDial: dialCodeOfCode(profile.countryCode), mobile: '', department: '', title: '', instantMessaging: '', language: '',
  emailPermission: 'ALLOWED', emailCategories: [] as string[], remark: '', isPrimary,
})
const newOwner = () => ({ employeeId: '', responsibilityCode: 'SALES', isPrimary: false, startDate: '', endDate: '' })

let nextRowKey = 0
function pending<T>(data: T) {
  return { data, saved: false, key: ++nextRowKey, session: newIdempotencySession() }
}
type Pending<T> = ReturnType<typeof pending<T>>
const addresses = ref<Pending<ReturnType<typeof newAddress>>[]>([])
const contacts = ref<Pending<ReturnType<typeof newContact>>[]>([])
const owners = ref<Pending<ReturnType<typeof newOwner>>[]>([])
const files = ref<Pending<{ file: File; reminderDate: string; error: string }>[]>([])
const recentFileKeys = ref(new Set<number>())

interface Option { code: string; label: string }
interface Employee { id: string | number; name: string; departmentName?: string }
const options = reactive({ customerTypes: [] as Option[], sources: [] as Option[], paymentMethods: [] as Option[], responsibilities: [] as Option[] })
const responsibilityOptions = computed(() => options.responsibilities.length ? options.responsibilities : [{ code: 'SALES', label: '销售' }])
const countries = computed(() => countryOptions(locale.value))
const timezones = computed(() => portTimezoneOptions(profile.countryCode))
const canReadEmployees = computed(() => auth.can('iam:employee:read'))
const canChooseEmployees = computed(() => canReadEmployees.value || props.canManageOwners)
const employees = ref<Employee[]>([])
const loadingEmployees = ref(false)
const fileInput = ref<HTMLInputElement>()
const scrollArea = ref<HTMLElement>()
const customerId = ref('')
const locked = computed(() => Boolean(customerId.value))
const saving = ref(false)
const basicSaved = ref(false)
const restoreAddress = ref(false)
const checkedDuplicates = ref(false)
const onlyFilesPending = ref(false)
const saveError = ref('')
const submitLabel = computed(() => onlyFilesPending.value ? '重试附件' : locked.value ? '继续保存' : '保存客户')
const createSession = newIdempotencySession()
const basicSession = newIdempotencySession()
const addressSnapshotSession = newIdempotencySession()
const createSnapshot = shallowRef<ReturnType<typeof primaryFields>>()

function resetForm() {
  Object.assign(profile, emptyProfile())
  Object.assign(company, emptyCompany())
  creditAmount.value = undefined
  addresses.value = []
  contacts.value = []
  owners.value = []
  files.value = []
  recentFileKeys.value.clear()
  customerId.value = ''
  basicSaved.value = false
  restoreAddress.value = false
  checkedDuplicates.value = false
  onlyFilesPending.value = false
  saveError.value = ''
  createSnapshot.value = undefined
  for (const session of [createSession, basicSession, addressSnapshotSession]) session.reset()
}

async function loadOptions() {
  const categories = {
    customerTypes: 'CUSTOMER_TYPE', sources: 'CUSTOMER_SOURCE',
    paymentMethods: 'PAYMENT_METHOD', responsibilities: 'CUSTOMER_OWNER_RESPONSIBILITY',
  } as const
  await Promise.allSettled((Object.keys(categories) as (keyof typeof categories)[]).map(async (key) => {
    options[key] = (await get<{ options: Option[] }>('/options', { category: categories[key] })).options || []
  }))
}
watch(() => props.open, (open) => {
  if (!open) return
  resetForm()
  void loadOptions()
  void nextTick(() => scrollArea.value?.scrollTo({ top: 0 }))
}, { immediate: true })

function jump(section: string) {
  scrollArea.value?.querySelector('#new-customer-' + section)?.scrollIntoView({ block: 'start', behavior: 'smooth' })
}
function addAddress() {
  addresses.value.push(pending(newAddress(!addresses.value.some((row) => row.data.addressType === 'OFFICE'))))
}
function addContact() {
  contacts.value.push(pending(newContact(!contacts.value.length)))
}
async function addOwner() {
  if (loadingEmployees.value) return
  if (!employees.value.length) {
    loadingEmployees.value = true
    try {
      const res = canReadEmployees.value
        ? await get<{ employees: Employee[] }>('/employees', { page: 1, page_size: 200, employment_status: 'ACTIVE' })
        : await get<{ employees: Employee[] }>('/customers/owner-options')
      employees.value = res.employees || []
    } catch {
      return
    } finally {
      loadingEmployees.value = false
    }
  }
  if (props.open) owners.value.push(pending(newOwner()))
}
function setDefaultAddress(row: Pending<ReturnType<typeof newAddress>>, checked: boolean) {
  if (checked) {
    for (const other of addresses.value) {
      if (other.data.addressType === row.data.addressType) other.data.isDefault = false
    }
  }
  row.data.isDefault = checked
}
function setPrimary<T extends { isPrimary: boolean }>(rows: Pending<T>[], row: Pending<T>, checked: boolean) {
  if (checked) {
    for (const other of rows) {
      other.data.isPrimary = false
    }
  }
  row.data.isPrimary = checked
}
async function selectFiles(event: Event) {
  const input = event.target as HTMLInputElement
  const added = []
  for (const file of Array.from(input.files || [])) {
    if (!file.size || file.size > 10 * 1024 * 1024) {
      ElMessage.warning(file.name + ' 为空或超过 10 MB')
      continue
    }
    added.push(pending({ file, reminderDate: '', error: '' }))
  }
  input.value = ''
  if (!added.length) return
  files.value.push(...added)
  recentFileKeys.value = new Set(added.map((row) => row.key))
  ElMessage.success('已添加 ' + added.length + ' 个附件，保存客户后上传')
  await nextTick()
  scrollArea.value?.querySelector('#new-customer-file-' + added[0].key)?.scrollIntoView({ block: 'start' })
}
function contactPhone(data: ReturnType<typeof newContact>) {
  const { phoneDial } = data
  const phone = data.phone.trim()
  return phone && phoneDial && !phone.startsWith('+') ? phoneDial + ' ' + phone : phone
}
function validate(): boolean {
  const report = (section: string, message: string) => {
    saveError.value = message
    jump(section)
    return false
  }
  if (!profile.name.trim()) return report('profile', '请填写客户名称')
  const profileError = validateCustomerProfile(profile)
  if (profileError) return report('profile', t('customers.' + profileError))
  if (company.companyEmail.trim() && validateCustomerContact({ name: profile.name, email: company.companyEmail })) {
    return report('profile', '公司邮箱格式不正确')
  }
  if (!Number.isSafeInteger(Math.round((creditAmount.value ?? 0) * 100))) {
    return report('credit', '信用额度超出可保存范围')
  }
  for (const [index, row] of addresses.value.entries()) {
    if (!row.data.addressLine.trim()) return report('address', '请填写地址 ' + (index + 1) + ' 的详细地址')
  }
  for (const [index, row] of contacts.value.entries()) {
    const error = validateCustomerContact({ ...row.data, phone: contactPhone(row.data) })
    if (error === 'nameRequired') return report('contacts', '联系人 ' + (index + 1) + ' 的姓名和邮箱至少填写一项')
    if (error) return report('contacts', '联系人 ' + (index + 1) + '：' + t('customers.' + error))
  }
  const ownerKeys = new Set([auth.employeeId + ':SALES'])
  for (const row of owners.value) {
    const { employeeId, responsibilityCode, startDate, endDate } = row.data
    if (!employeeId || !responsibilityCode) return report('owners', '请为每位负责人选择员工和职责')
    if (startDate && endDate && endDate < startDate) return report('owners', '结束日期不能早于生效日期')
    const key = employeeId + ':' + responsibilityCode
    if (ownerKeys.has(key)) return report('owners', '同一员工的同一职责只需填写一次，当前账号已默认负责销售')
    ownerKeys.add(key)
  }
  return true
}
function primaryFields() {
  const offices = addresses.value.filter((row) => row.data.addressType === 'OFFICE')
  const address = (offices.find((row) => row.data.isDefault) ?? offices[0] ?? addresses.value[0])?.data.addressLine.trim() || ''
  return {
    name: profile.name.trim(), countryCode: profile.countryCode,
    country: profile.countryCode ? countryName(profile.countryCode, locale.value) : '',
    address, currency: profile.currency, paymentTerm: profile.paymentTerm || '', remark: profile.remark,
  }
}
function errorText(error: unknown) {
  const response = error as { response?: { data?: { message?: string } }; message?: string }
  return response.response?.data?.message || response.message || '请重试'
}

async function saveRows<T>(rows: Pending<T>[], url: string, label: string, body: (data: T) => object) {
  for (const [index, row] of rows.entries()) {
    if (row.saved) continue
    try {
      await post(url, body(row.data), withIdempotency(row.session, quietErrors))
      row.saved = true
    } catch (error) {
      throw new Error(label + ' ' + (index + 1) + ' 保存失败：' + errorText(error))
    }
  }
}

async function saveDocuments(url: string) {
  for (const row of files.value) {
    if (row.saved) continue
    const { file, reminderDate } = row.data
    const body = new FormData()
    body.set('title', file.name)
    body.set('expiresOn', reminderDate || '')
    body.set('remindDays', '0')
    body.set('reminderEnabled', String(Boolean(reminderDate)))
    body.set('file', file)
    row.data.error = ''
    try {
      await http.post(url, body, withIdempotency(row.session, { ...quietErrors, timeout: 120000 }))
      row.saved = true
      announceReminderChanged()
    } catch (error) {
      row.data.error = '上传失败：' + errorText(error)
    }
  }
}

async function save() {
  if (saving.value || !auth.can('masterdata:customer:write') || !validate()) return
  saving.value = true
  saveError.value = ''
  try {
    if (!customerId.value && !checkedDuplicates.value) {
      const { candidates } = await get<{ candidates: DuplicateCandidate[] }>('/customers/duplicates', {
        name: profile.name.trim(), email: contacts.value.find((row) => row.data.isPrimary)?.data.email || contacts.value[0]?.data.email,
        tax_id: profile.taxId.trim() || undefined,
      })
      try {
        await confirmPossibleDuplicates(candidates || [], t)
      } catch {
        return
      }
      checkedDuplicates.value = true
    }
    if (!customerId.value) {
      const main = primaryFields()
      const { customer } = await post<{ customer: { id: string } }>('/customers', {
        ...profile, ...main, creditLimitMinor: String(Math.round((creditAmount.value ?? 0) * 100)),
      }, withIdempotency(createSession, quietErrors))
      customerId.value = customer.id
      createSnapshot.value = main
    }

    const url = '/customers/' + customerId.value
    const main = createSnapshot.value!
    if (!basicSaved.value) {
      if (Object.values(company).some((value) => value.trim())) {
        await put(url + '/basic', { basic: {
          ...company, code: profile.code, name: main.name, shortName: profile.shortName,
          countryRegion: main.country, countryCode: main.countryCode,
          customerType: profile.customerType || '', source: profile.source || '',
        } }, withIdempotency(basicSession, quietErrors))
        restoreAddress.value = Boolean(main.address)
      }
      basicSaved.value = true
    }
    if (restoreAddress.value) {
      // The basic editor syncs the legacy communication address. Restore that
      // value before writing structured rows so it won't create a duplicate office address.
      await put(url, main, withIdempotency(addressSnapshotSession, quietErrors))
      restoreAddress.value = false
    }
    await saveRows(addresses.value, url + '/addresses', '地址', (data) => ({ address: customerAddressInput(data) }))
    await saveRows(contacts.value, url + '/contacts', '联系人', (data) => ({
      contact: customerContactInput({ ...data, phone: contactPhone(data) }),
    }))
    await saveRows(owners.value, url + '/owners', '负责人', (data) => ({
      owner: { ...data, startDate: data.startDate || '', endDate: data.endDate || '' },
    }))

    await saveDocuments(url + '/documents')
    const failures = files.value.filter((row) => !row.saved).length
    if (failures) {
      onlyFilesPending.value = true
      saveError.value = '客户资料已保存。' + failures + ' 个附件上传失败，可在这里重试。'
      jump('documents')
      return
    }
    emit('created')
    emit('update:open', false)
    ElMessage.success(t('customers.created'))
  } catch (error) {
    if (!customerId.value && error && (error as { response?: { status?: number } }).response?.status === 400) {
      checkedDuplicates.value = false
    }
    saveError.value = (customerId.value ? '客户已建档，可继续保存剩余资料。' : '保存客户失败。') + errorText(error)
  } finally {
    saving.value = false
  }
}

async function confirmClose(done: () => void) {
  if (saving.value) return
  if (customerId.value) {
    try {
      await ElMessageBox.confirm('已保存的资料会保留，关闭后未保存的内容需要重新录入。', '关闭新建客户', {
        type: 'warning', confirmButtonText: '关闭', cancelButtonText: '继续填写',
      })
    } catch {
      return
    }
    emit('created')
  }
  done()
}
</script>

<style scoped>
.create-scroll {
  max-height: calc(92dvh - 118px);
  overflow: auto;
  padding-inline: 2px 12px;
}
.create-nav {
  position: sticky;
  top: 0;
  z-index: 2;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 3px 8px;
  padding: 5px 0;
  border-bottom: 1px solid #dce7f4;
  background: #fff;
}
.create-nav button {
  padding: 5px 8px;
  border: 0;
  border-radius: 4px;
  color: #205573;
  background: transparent;
  cursor: pointer;
}
.create-nav button:hover,
.create-nav button:focus-visible { background: #edf6fb; }
.create-nav span { margin-inline-start: auto; font-size: 12px; color: var(--el-text-color-secondary); }
.create-section { padding: 14px 0 4px; scroll-margin-top: 62px; }
.create-section + .create-section { border-top: 1px solid #dce7f4; }
.create-section h3 { margin: 0 0 9px; font-size: 16px; color: #173b52; }
.section-heading { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; }
.fields { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); column-gap: 18px; }
.full { grid-column: 1 / -1; }
.fields :deep(.el-form-item) { min-width: 0; margin-bottom: 10px; }
.fields :deep(.el-input-number),
.fields :deep(.el-select),
.fields :deep(.el-date-editor) { width: 100%; }
.fields :deep(.el-input-number input) { text-align: left; }
fieldset { min-width: 0; margin: 0 0 12px; padding: 8px 12px 0; border: 1px solid #dce7f4; border-radius: 6px; }
legend { padding-inline: 5px; font-size: 13px; color: #205573; }
.row-actions, .upload-row, .create-footer { display: flex; align-items: center; gap: 8px; }
.row-actions { flex-wrap: wrap; }
.phone-field { display: grid; width: 100%; grid-template-columns: 86px minmax(0, 1fr); gap: 8px; }
.attachment-count { margin-inline-start: 6px; color: var(--el-text-color-secondary); font-size: 13px; font-weight: 400; }
.upload-row {
  flex-wrap: wrap;
  min-height: 36px;
  margin-bottom: 6px;
  padding: 4px 8px;
  border-radius: 5px;
  scroll-margin-top: 76px;
}
.upload-row--new { background: #edf6fb; }
.upload-row .filename { min-width: min(240px, 100%); overflow-wrap: anywhere; }
.attachment-reminder { display: flex; align-items: center; flex-wrap: wrap; gap: 4px 8px; }
.attachment-reminder label { color: var(--el-text-color-regular); font-size: 13px; }
.attachment-reminder :deep(.el-date-editor) { width: 150px; }
.create-footer { justify-content: flex-end; flex-wrap: wrap; }
.create-footer .el-button + .el-button { margin-left: 0; }
.create-footer p { flex: 1 1 240px; margin: 0; text-align: start; }
.muted { margin: 0 0 10px; color: var(--el-text-color-secondary); font-size: 13px; }
.error-text { font-size: 13px; color: var(--el-color-danger-dark-2); }
.saved-mark { font-size: 12px; color: var(--el-color-success-dark-2); }
@media (max-width: 800px) {
  .fields { grid-template-columns: repeat(2, minmax(0, 1fr)); column-gap: 12px; }
}
@media (max-width: 500px) {
  .fields { grid-template-columns: minmax(0, 1fr); }
  .create-section, .upload-row { scroll-margin-top: 100px; }
}
</style>
