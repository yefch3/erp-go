export type CustomerContactValidationError = 'nameRequired' | 'emailInvalid' | 'phoneInvalid'
export type CustomerProfileValidationError = 'websiteInvalid' | 'timezoneInvalid'

export interface CustomerContactFormInput {
  name: string
  email?: string
  phone?: string
  mobile?: string
}

export interface CustomerProfileFormInput {
  website?: string
  timezone?: string
}

const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
const phonePattern = /^[0-9+()\-. ]{6,30}$/

// 客户列表和详情页共用同一套联系人校验，避免两个入口的规则不一致。
export function validateCustomerContact(form: CustomerContactFormInput): CustomerContactValidationError | null {
  if (!form.name.trim() && !form.email?.trim()) return 'nameRequired'
  const email = form.email?.trim() ?? ''
  if (email && !emailPattern.test(email)) return 'emailInvalid'
  for (const value of [form.phone, form.mobile]) {
    const phone = value?.trim() ?? ''
    if (phone && !phonePattern.test(phone)) return 'phoneInvalid'
  }
  return null
}

// 请求体只能带接口定义里有的字段。
//
// 网关按 proto 严格解析 JSON，多一个字段就整个拒收（「unknown field "id"」）。
// 编辑弹窗的表单是从列表那一行整个复制来的，行上带着 id、status 这种只回显
// 不回传的字段——新增时表单是空的所以没事，编辑时一定带、一定被拒。这两张
// 表和 proto 里的 CustomerContactInput / CustomerAddressInput 一一对应。
const contactInputFields = [
  'name',
  'department',
  'title',
  'email',
  'phone',
  'mobile',
  'instantMessaging',
  'language',
  'remark',
  'isPrimary',
  'sortOrder',
  'emailPermission',
  'emailCategories',
] as const
const addressInputFields = [
  'addressType',
  'countryCode',
  'state',
  'city',
  'postalCode',
  'addressLine',
  'isDefault',
  'sortOrder',
] as const

export type CustomerContactInputField = (typeof contactInputFields)[number]
export type CustomerAddressInputField = (typeof addressInputFields)[number]

function pick<K extends string>(form: Record<string, unknown>, keys: readonly K[]): Partial<Record<K, unknown>> {
  const out: Partial<Record<K, unknown>> = {}
  for (const key of keys) {
    if (key in form) out[key] = form[key]
  }
  return out
}

export function customerContactInput(form: Record<string, unknown>): Partial<Record<CustomerContactInputField, unknown>> {
  return pick(form, contactInputFields)
}

export function customerAddressInput(form: Record<string, unknown>): Partial<Record<CustomerAddressInputField, unknown>> {
  return pick(form, addressInputFields)
}

// 官网只允许 http(s)，时区使用浏览器支持的 IANA 时区名（例如 Asia/Shanghai）。
export function validateCustomerProfile(form: CustomerProfileFormInput): CustomerProfileValidationError | null {
  const website = form.website?.trim() ?? ''
  if (website) {
    try {
      const parsed = new URL(website)
      if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return 'websiteInvalid'
    } catch {
      return 'websiteInvalid'
    }
  }

  const timezone = form.timezone?.trim() ?? ''
  if (timezone) {
    try {
      new Intl.DateTimeFormat('zh-CN', { timeZone: timezone }).format()
    } catch {
      return 'timezoneInvalid'
    }
  }
  return null
}
