export interface CustomerDisplayFields {
  code?: string
  name?: string
  shortName?: string
}

// 业务界面统一优先显示客户简称；未维护简称的旧客户才回退客户名称。
export function customerDisplayName(customer?: CustomerDisplayFields | null): string {
  return customer?.shortName?.trim() || customer?.name?.trim() || ''
}

export function customerOptionLabel(customer?: CustomerDisplayFields | null): string {
  if (!customer) return ''
  const code = customer.code?.trim() || ''
  const display = customerDisplayName(customer)
  const fullName = customer.name?.trim() || ''
  const detail = customer.shortName?.trim() && fullName && fullName !== display ? `（${fullName}）` : ''
  return [code, `${display}${detail}`].filter(Boolean).join(' · ')
}
