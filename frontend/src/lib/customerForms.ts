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
  if (!form.name.trim()) return 'nameRequired'
  const email = form.email?.trim() ?? ''
  if (email && !emailPattern.test(email)) return 'emailInvalid'
  for (const value of [form.phone, form.mobile]) {
    const phone = value?.trim() ?? ''
    if (phone && !phonePattern.test(phone)) return 'phoneInvalid'
  }
  return null
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
