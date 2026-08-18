// 询盘列模板页面的纯逻辑：字段行操作与保存前校验。规则与 procurement 服务
// 保持一致（核心字段不可删且必填、表头与字段标识不得重复），服务端仍是
// 最终裁决者，这里只是为了在保存前给出即时反馈。

export interface TemplateField {
  fieldKey: string
  displayName: string
  sortOrder: number
  isRequired: boolean
  defaultValue: string
  dataType: string // TEXT | NUMBER | DATE
  isCustom: boolean
  isCore: boolean
}

export interface InquiryTemplate {
  id: string
  templateCode: string
  version: number
  name: string
  description: string
  status: string
  isDefault: boolean
  isSystem: boolean
  fieldCount: number
  fields?: TemplateField[]
  createdByName: string
  updatedAt: string
}

// 核心字段：任何模板都不得删除。其中产品/数量/单位强制必填；单价/总价在
// 询盘阶段留空（价格由工厂报价产生），只作固定列。
export const coreFieldKeys = ['product', 'quantity', 'quantity_unit', 'unit_price', 'total_price']
export const requiredFieldKeys = ['product', 'quantity', 'quantity_unit']

export const fieldKeyPattern = /^[a-z][a-z0-9_]{0,79}$/

export function moveField(fields: TemplateField[], index: number, direction: -1 | 1): void {
  const target = index + direction
  if (index < 0 || target < 0 || target >= fields.length) return
  const [row] = fields.splice(index, 1)
  fields.splice(target, 0, row)
  fields.forEach((field, i) => { field.sortOrder = i + 1 })
}

// 新自定义列的默认标识：custom.column_N，取当前未被占用的最小序号。
export function nextCustomFieldKey(fields: TemplateField[]): string {
  const used = new Set(fields.map((field) => field.fieldKey))
  for (let n = 1; ; n++) {
    const key = `custom.column_${n}`
    if (!used.has(key)) return key
  }
}

// 返回 locale 错误键；null 表示可以提交。
export function validateTemplateFields(fields: TemplateField[]): string | null {
  if (!fields.length) return 'inquiryTemplates.errors.fieldsRequired'
  const keys = new Set<string>()
  const names = new Set<string>()
  for (const field of fields) {
    if (!field.displayName.trim()) return 'inquiryTemplates.errors.nameRequired'
    if (!fieldKeyPattern.test(field.fieldKey) && !field.isCustom) return 'inquiryTemplates.errors.keyInvalid'
    if (field.isCustom && !/^custom\.[a-z][a-z0-9_]{0,79}$/.test(field.fieldKey)) return 'inquiryTemplates.errors.keyInvalid'
    if (field.isCore && field.isRequired && !requiredFieldKeys.includes(field.fieldKey)) return 'inquiryTemplates.errors.priceRequired'
    if (keys.has(field.fieldKey)) return 'inquiryTemplates.errors.keyDuplicate'
    const name = field.displayName.trim().toLowerCase()
    if (names.has(name)) return 'inquiryTemplates.errors.nameDuplicate'
    keys.add(field.fieldKey)
    names.add(name)
  }
  for (const core of coreFieldKeys) {
    if (!keys.has(core)) return 'inquiryTemplates.errors.coreMissing'
  }
  return null
}
