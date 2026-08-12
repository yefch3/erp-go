import { describe, expect, it } from 'vitest'
import { validateCustomerContact, validateCustomerProfile } from './customerForms'

describe('客户联系人校验', () => {
  it('拒绝空姓名、错误邮箱和错误电话', () => {
    expect(validateCustomerContact({ name: ' ' })).toBe('nameRequired')
    expect(validateCustomerContact({ name: 'Alice', email: 'alice@' })).toBe('emailInvalid')
    expect(validateCustomerContact({ name: 'Alice', phone: 'call-me' })).toBe('phoneInvalid')
  })

  it('接受常见国际电话和合法邮箱', () => {
    expect(validateCustomerContact({ name: 'Alice', email: 'alice@example.com', phone: '+1 (212) 555-0100' })).toBeNull()
  })
})

describe('客户详细资料校验', () => {
  it('拒绝非 http(s) 官网和无效时区', () => {
    expect(validateCustomerProfile({ website: 'example.com' })).toBe('websiteInvalid')
    expect(validateCustomerProfile({ timezone: 'Shanghai' })).toBe('timezoneInvalid')
  })

  it('接受合法官网和 IANA 时区', () => {
    expect(validateCustomerProfile({ website: 'https://example.com', timezone: 'Asia/Shanghai' })).toBeNull()
  })
})
