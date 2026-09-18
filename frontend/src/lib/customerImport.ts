import { COUNTRY_CODES, countryOptions } from './countries'

export const CUSTOMER_IMPORT_FIELDS = [
  { key: 'code', label: '客户编码', aliases: ['编码', '客户代码', '客户编码', '客户编号', 'code', 'customer code', 'customer no'] },
  { key: 'name', label: '客户名称', required: true, aliases: ['名称', '客户名称', '公司名称', '公司', 'name', 'customer name', 'company', 'company name'] },
  { key: 'countryCode', label: '国家代码', aliases: ['国家代码', '国家编码', '国家', '地区代码', 'country code', 'country', 'country/region'] },
  { key: 'countryRegion', label: '国家/地区', aliases: ['所属地区', '国家/地区', '国家地区', 'country region', 'region'] },
  { key: 'customerType', label: '客户类型', aliases: ['客户类型', '类型', 'customer type', 'type'] },
  { key: 'currency', label: '币种', aliases: ['币种', '货币', 'currency'] },
  { key: 'paymentTerm', label: '付款方式', aliases: ['付款方式', '付款条件', 'payment term', 'payment terms'] },
  { key: 'contactName', label: '联系人姓名', aliases: ['联系人', '联系姓名', '联系人姓名', 'contact', 'contact name'] },
  { key: 'contactEmail', label: '联系人邮箱', aliases: ['邮箱', '电子邮箱', '联系人邮箱', 'contact email', 'email', 'e-mail'] },
  { key: 'contactPhone', label: '联系人电话', aliases: ['电话', '联系人电话', 'contact phone', 'phone', 'telephone'] },
  { key: 'contactMobile', label: '联系人手机', aliases: ['联系人手机', '联系人手机号', 'contact mobile', 'mobile'] },
  { key: 'remark', label: '备注', aliases: ['备注', '说明', 'remark', 'remarks', 'note', 'notes'] },
  { key: 'address', label: '通信地址', aliases: ['地址', '通信地址', '公司地址', '办公地址', 'address'] },
  { key: 'postalCode', label: '邮政编码', aliases: ['邮政编码', '邮编', 'postal code', 'zip', 'zip code'] },
  { key: 'shortName', label: '客户简称', aliases: ['客户简称', '简称', 'short name'] },
  { key: 'englishName', label: '英文名称', aliases: ['英文名称', '公司英文名', 'english name'] },
  { key: 'industry', label: '所属行业', aliases: ['所属行业', '行业', 'industry'] },
  { key: 'source', label: '客户来源', aliases: ['客户来源', '来源', 'source'] },
  { key: 'tags', label: '标签', aliases: ['标签', '客户标签', 'tags'] },
  { key: 'website', label: '官网', aliases: ['官网', '网站', 'website', 'web site'] },
  { key: 'primaryLanguage', label: '主要语言', aliases: ['主要语言', '语言', 'language', 'primary language'] },
  { key: 'timezone', label: '所在时区', aliases: ['所在时区', '时区', 'timezone', 'time zone'] },
  { key: 'registeredName', label: '注册名称', aliases: ['注册名称', '工商注册名称', 'registered name'] },
  { key: 'registrationNo', label: '公司注册号', aliases: ['公司注册号', '注册号', 'registration no', 'registration number'] },
  { key: 'taxId', label: '税号', aliases: ['税号', '税务编号', 'tax id', 'tax number'] },
  { key: 'invoiceTitle', label: '开票抬头', aliases: ['开票抬头', '发票抬头', 'invoice title'] },
  { key: 'invoiceTaxNo', label: '开票税号', aliases: ['开票税号', '发票税号', 'invoice tax no'] },
  { key: 'invoiceRemark', label: '开票备注', aliases: ['开票备注', '发票备注', 'invoice remark'] },
  { key: 'businessStatus', label: '业务状态', aliases: ['业务状态', '合作状态', 'business status'] },
  { key: 'creditGrade', label: '客户等级', aliases: ['客户等级', '信用等级', 'credit grade', 'customer grade'] },
  { key: 'ownerName', label: '负责人', aliases: ['分管人', '负责人', '业务负责人', 'owner', 'account owner'] },
] as const

export type CustomerImportField = typeof CUSTOMER_IMPORT_FIELDS[number]['key']
export interface CustomerFieldDefinition { fieldKey: string; displayName: string; aliases?: string[]; sortOrder?: number }
export type CustomerImportMapping = Record<number, string>
export interface CustomerImportEmployee { id: string | number; name: string; code?: string; email?: string }
export interface CustomerImportRow extends Partial<Record<CustomerImportField, string>> {
  addressState?: string
  ownerEmployeeId?: string
  customFields?: Record<string, string>
}
export interface CustomerImportFieldMapping { sourceKey: string; fieldKey: string; displayName: string; aliases: string[] }

export function normalizeCustomerHeader(value: string): string {
  return value.trim().toLocaleLowerCase().replace(/[\s_\-—–/\\（）()【】\[\]：:。.]+/g, '')
}

export function isSystemCustomerFieldDefinition(field: Pick<CustomerFieldDefinition, 'displayName' | 'aliases'>): boolean {
  const names = [field.displayName, ...(field.aliases ?? [])]
    .map(normalizeCustomerHeader)
    .filter(Boolean)
  return CUSTOMER_IMPORT_FIELDS.some(systemField => {
    const systemNames = [systemField.label, ...systemField.aliases].map(normalizeCustomerHeader)
    return names.some(name => systemNames.includes(name))
  })
}

export function extensionCustomerFields(fields: CustomerFieldDefinition[]): CustomerFieldDefinition[] {
  return fields.filter(field => !isSystemCustomerFieldDefinition(field))
}

export function autoMapCustomerHeaders(headers: string[], customFields: CustomerFieldDefinition[] = []): CustomerImportMapping {
  const extensionFields = extensionCustomerFields(customFields)
  const mapping: CustomerImportMapping = {}
  const used = new Set<string>()
  headers.forEach((header, index) => {
    const normalized = normalizeCustomerHeader(header)
    const fixed = CUSTOMER_IMPORT_FIELDS.find(candidate => candidate.aliases.some(alias => normalizeCustomerHeader(alias) === normalized))
    let target = fixed ? `system:${fixed.key}` : ''
    if (!target) {
      const custom = extensionFields.find(field => [field.displayName, ...(field.aliases ?? [])].some(alias => normalizeCustomerHeader(alias) === normalized))
      if (custom) target = `custom:${custom.fieldKey}`
    }
    if (!target) target = `new:${index}`
    mapping[index] = used.has(target) ? `new:${index}` : target
    used.add(mapping[index])
  })
  return mapping
}

export function customerImportMappingProblems(mapping: CustomerImportMapping, newFieldNames: Record<number, string> = {}): string[] {
  const selected = Object.values(mapping).filter(Boolean)
  const problems: string[] = []
  if (!selected.includes('system:name')) problems.push('请选择一列作为“客户名称”')
  const duplicate = selected.find((field, index) => !field.startsWith('new:') && selected.indexOf(field) !== index)
  if (duplicate) problems.push('同一个 ERP 字段只能对应一个 Excel 列')
  const newNames = new Set<string>()
  for (const [column, target] of Object.entries(mapping)) {
    if (!target.startsWith('new:')) continue
    const name = newFieldNames[Number(column)]?.trim()
    if (!name) problems.push('新建的客户字段必须填写显示名称')
    const normalized = normalizeCustomerHeader(name ?? '')
    if (normalized && newNames.has(normalized)) problems.push('新建客户字段的显示名称不能重复')
    if (normalized) newNames.add(normalized)
  }
  return [...new Set(problems)]
}

export function resolveImportedCountry(value: string): { countryCode: string; state: string } {
  const source = value.trim()
  if (!source) return { countryCode: '', state: '' }
  const upper = source.toUpperCase()
  if (COUNTRY_CODES.includes(upper)) return { countryCode: upper, state: '' }
  const candidates = ['zh-CN', 'en-US', 'es'].flatMap(locale => countryOptions(locale))
    .sort((a, b) => b.name.length - a.name.length)
  const match = candidates.find(option => source === option.name
    || source.startsWith(`${option.name}·`)
    || source.startsWith(`${option.name}/`)
    || source.startsWith(`${option.name},`)
    || source.startsWith(`${option.name}，`))
  if (!match) return { countryCode: source, state: '' }
  return { countryCode: match.code, state: source.slice(match.name.length).replace(/^[·/,，\s-]+/, '').trim() }
}

function normalizedPerson(value: string): string {
  return value.trim().toLocaleLowerCase().replace(/\s+/g, '')
}

export function buildCustomerImportRows(sourceRows: string[][], mapping: CustomerImportMapping, employees: CustomerImportEmployee[] = []): CustomerImportRow[] {
  return sourceRows.map(sourceRow => {
    const row: CustomerImportRow = {}
    const customFields: Record<string, string> = {}
    Object.entries(mapping).forEach(([columnText, target]) => {
      if (!target) return
      const column = Number(columnText)
      const value = (sourceRow[column] ?? '').trim()
      if (target === 'system:countryRegion') {
        const resolved = resolveImportedCountry(value)
        row.countryCode = resolved.countryCode
        row.addressState = resolved.state
      } else if (target.startsWith('system:')) row[target.slice(7) as CustomerImportField] = value
      else customFields[`column_${column}`] = value
    })
    if (row.ownerName) {
      const key = normalizedPerson(row.ownerName)
      const matches = employees.filter(employee => [employee.name, employee.code ?? '', employee.email ?? ''].some(value => normalizedPerson(value) === key))
      if (matches.length === 1) row.ownerEmployeeId = String(matches[0].id)
    }
    if (Object.keys(customFields).length) row.customFields = customFields
    return row
  }).filter(row => Object.entries(row).some(([key, value]) => key === 'customFields' ? Object.values(value as Record<string, string>).some(Boolean) : Boolean(value)))
}

export function buildCustomerImportFieldMappings(headers: string[], mapping: CustomerImportMapping, newFieldNames: Record<number, string>, customFields: CustomerFieldDefinition[]): CustomerImportFieldMapping[] {
  return Object.entries(mapping).flatMap(([columnText, target]) => {
    if (!target || target.startsWith('system:')) return []
    const column = Number(columnText)
    const existing = target.startsWith('custom:') ? customFields.find(field => field.fieldKey === target.slice(7)) : undefined
    return [{ sourceKey: `column_${column}`, fieldKey: existing?.fieldKey ?? '', displayName: existing?.displayName ?? newFieldNames[column]?.trim() ?? headers[column]?.trim() ?? '', aliases: [headers[column]?.trim() ?? ''].filter(Boolean) }]
  })
}
