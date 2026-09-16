import { describe, expect, it } from 'vitest'
import {
  customerAddressInput,
  customerContactInput,
  validateCustomerContact,
  validateCustomerProfile,
} from './customerForms'

// 编辑弹窗的表单是从列表那一行整个复制来的，行上带着 id、status 这种只回显
// 不回传的字段。网关按接口定义严格解析请求体，多一个字段就整个拒收，所以
// 发出去之前只能留接口认的那几项。
describe('客户联系人请求体', () => {
  const row = {
    id: '7',
    status: 'ACTIVE',
    name: '崔骏',
    department: '',
    title: '',
    email: 'cui@example.com',
    phone: '+86 1324564580',
    mobile: '',
    instantMessaging: '',
    language: '',
    remark: '',
    isPrimary: true,
    sortOrder: 0,
    emailPermission: 'ALLOWED',
    emailCategories: ['QUOTE'],
  }

  it('编辑时去掉列表行带来的 id 和 status，其余字段原样带上', () => {
    const { id: _id, status: _status, ...expected } = row
    expect(customerContactInput(row)).toEqual(expected)
  })

  it('新增时表单本来就没有那些字段，结果一样', () => {
    const fresh = { name: 'Alice', email: '', isPrimary: false, emailCategories: [] }
    expect(customerContactInput(fresh)).toEqual(fresh)
  })
})

describe('客户地址请求体', () => {
  it('编辑时去掉 id、customerId 和 status', () => {
    const row = {
      id: '3',
      customerId: '5',
      status: 'ACTIVE',
      addressType: 'OFFICE',
      countryCode: 'CN',
      state: '',
      city: '上海',
      postalCode: '',
      addressLine: '南京路 1 号',
      isDefault: true,
      sortOrder: 0,
    }
    const { id: _id, customerId: _cid, status: _status, ...expected } = row
    expect(customerAddressInput(row)).toEqual(expected)
  })
})

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
