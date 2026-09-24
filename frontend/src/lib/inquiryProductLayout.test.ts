import { describe, expect, it } from 'vitest'
import { inquiryProductFields, planInquiryColumns } from './inquiryProductLayout'
import type { TemplateField } from './inquiryTemplates'
import type { Product } from './inquiryWorkspace'

const field = (fieldKey: string, sortOrder: number): TemplateField => ({
  fieldKey, displayName: fieldKey, sortOrder, isRequired: ['product', 'quantity', 'quantity_unit'].includes(fieldKey),
  defaultValue: '', dataType: fieldKey === 'quantity' ? 'NUMBER' : 'TEXT', isCustom: fieldKey.startsWith('custom.'), isCore: false,
})

const product: Product = {
  id: '1', product: '矩形钢管', specification: '2 mm × 50 mm × 30 mm', quantity: '500', unit: 'PCS',
  delivery: '2026-10-20', weight: '', volume: '', packaging: '成捆出口包装', packageQuantity: '',
  remark: '表面需要热浸镀锌，包装应适合出口运输。',
  customFields: { material_standard: 'ASTM A500 Grade B', length_or_form: '6000', surface_requirement: '热浸镀锌' },
}

describe('inquiry product layout', () => {
  it('keeps template order and includes populated fields missing from the template', () => {
    const template = [field('product', 1), field('quantity', 2), field('quantity_unit', 3), field('custom.customer_part_no', 4)]
    const result = inquiryProductFields([product], template, false)
    expect(result.slice(0, 3).map(item => item.fieldKey)).toEqual(['product', 'quantity', 'quantity_unit'])
    expect(result.map(item => item.fieldKey)).toContain('material_standard')
    expect(result.map(item => item.fieldKey)).toContain('specification')
    expect(result.map(item => item.fieldKey)).toContain('remarks')
    expect(result.map(item => item.fieldKey)).not.toContain('custom.customer_part_no')
  })

  it('keeps all populated columns in one table and scrolls when their content does not fit', () => {
    const fields = inquiryProductFields([product], [field('product', 1), field('quantity', 2), field('quantity_unit', 3)], false)
    const plan = planInquiryColumns(fields, [product], {}, 700)
    expect(plan.columns.map(item => item.fieldKey)).toEqual(expect.arrayContaining(['product', 'quantity', 'quantity_unit']))
    expect(plan.columns.map(item => item.fieldKey)).toEqual(expect.arrayContaining(['specification', 'material_standard', 'remarks']))
    expect(plan.widths.quantity).toBeLessThan(plan.widths.product)
    expect(plan.widths.quantity_unit).toBeLessThan(plan.widths.product)
    expect(plan.totalWidth).toBeGreaterThan(700)
  })
})
