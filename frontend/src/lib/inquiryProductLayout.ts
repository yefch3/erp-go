import type { TemplateField } from './inquiryTemplates'
import { productTemplateValue, type Product } from './inquiryWorkspace'

const supplementalColumns: Array<[string, string, string?]> = [
  ['material_standard', '材质/标准'], ['grade', '牌号/等级'],
  ['specification', '规格'], ['thickness', '厚度(mm)', 'NUMBER'],
  ['custom.thickness_mm', '厚度(mm)', 'NUMBER'], ['custom.wall_thickness_mm', '壁厚(mm)', 'NUMBER'],
  ['width', '宽度(mm)', 'NUMBER'], ['custom.width_mm', '宽度(mm)', 'NUMBER'],
  ['custom.height_mm', '高度(mm)', 'NUMBER'], ['custom.diameter_mm', '直径(mm)', 'NUMBER'],
  ['custom.leg1_mm', '边长1(mm)', 'NUMBER'], ['custom.leg2_mm', '边长2(mm)', 'NUMBER'],
  ['custom.height_or_leg2', '高度/边长2(mm)', 'NUMBER'], ['length_or_form', '长度(mm)'],
  ['surface_requirement', '表面要求'], ['coating', '涂层/镀层'],
  ['tolerance', '公差'], ['coil_weight', '卷重(MT)'], ['coil_id', '卷内径(mm)'],
  ['delivery', '交期'], ['packaging', '包装'], ['package_quantity', '包装数量', 'NUMBER'],
  ['payment_terms', '付款条件'], ['incoterm', '贸易术语'], ['port', '港口'],
  ['weight', '重量', 'NUMBER'], ['volume', '体积', 'NUMBER'], ['remarks', '备注'],
]

const excluded = new Set(['unit_price', 'total_price'])
const keyFields = new Set(['product', 'quantity', 'quantity_unit'])

function supplementalField(key: string, name: string, type = 'TEXT'): TemplateField {
  return {
    fieldKey: key, displayName: name, sortOrder: 0, isRequired: false,
    defaultValue: '', dataType: type, isCustom: key.startsWith('custom.'), isCore: false,
  }
}

export function inquiryProductFields(products: Product[], templateFields: TemplateField[] | undefined, editable: boolean): TemplateField[] {
  const fields = (templateFields?.length ? templateFields : [
    supplementalField('product', '产品'), supplementalField('quantity', '数量', 'NUMBER'),
    supplementalField('quantity_unit', '单位'),
  ]).slice().sort((a, b) => a.sortOrder - b.sortOrder).filter(field => !excluded.has(field.fieldKey))
  const seen = new Set(fields.map(field => field.fieldKey))
  const hasValue = (key: string) => products.some(product => productTemplateValue(product, key).trim() !== '')

  for (const [key, name, type] of supplementalColumns) {
    if (!seen.has(key) && hasValue(key)) {
      fields.push(supplementalField(key, name, type))
      seen.add(key)
    }
  }
  for (const key of [...new Set(products.flatMap(product => Object.keys(product.customFields || {})))].sort()) {
    if (excluded.has(key) || seen.has(key) || !hasValue(key)) continue
    const label = key.replace(/^custom\./, '').replace(/_/g, ' ')
    fields.push(supplementalField(key, label))
    seen.add(key)
  }
  return editable ? fields : fields.filter(field => keyFields.has(field.fieldKey) || hasValue(field.fieldKey))
}

function visualLength(value: string): number {
  return Math.max(0, ...value.split('\n').map(line => Array.from(line).reduce((length, char) => length + (/[^\x00-\x7f]/.test(char) ? 2 : 1), 0)))
}

export function inquiryFieldWidth(field: TemplateField, products: Product[], label: string): number {
  const key = field.fieldKey
  if (key === 'quantity') return 94
  if (key === 'quantity_unit') return 104
  if (field.dataType === 'NUMBER') return 105
  if (field.dataType === 'DATE') return 128
  const longest = Math.max(visualLength(label), ...products.map(product => visualLength(productTemplateValue(product, key))))
  if (key === 'remarks') return Math.min(420, Math.max(250, longest * 7 + 20))
  if (key === 'product') return Math.min(270, Math.max(160, longest * 7 + 20))
  if (key === 'material_standard' || key === 'specification') return Math.min(360, Math.max(200, longest * 7 + 20))
  return Math.min(290, Math.max(110, longest * 7 + 20))
}

export interface InquiryColumnPlan {
  columns: TemplateField[]
  widths: Record<string, number>
  totalWidth: number
}

export function planInquiryColumns(fields: TemplateField[], products: Product[], labels: Record<string, string>, containerWidth: number, fixedWidth = 0): InquiryColumnPlan {
  const available = Math.max(350, containerWidth - fixedWidth)
  const natural = (field: TemplateField) => inquiryFieldWidth(field, products, labels[field.fieldKey] || field.displayName)
  const used = fields.reduce((total, field) => total + natural(field), 0)
  const widths = Object.fromEntries(fields.map(field => [field.fieldKey, natural(field)]))
  const remaining = Math.max(0, available - used)
  const growable = fields.filter(field => !['quantity', 'quantity_unit'].includes(field.fieldKey) && field.dataType !== 'NUMBER')
  const weight = growable.reduce((total, field) => total + natural(field), 0)
  if (remaining && weight) {
    for (const field of growable) widths[field.fieldKey] += remaining * natural(field) / weight
  }
  return { columns: fields, widths, totalWidth: Math.max(containerWidth, used + fixedWidth) }
}
